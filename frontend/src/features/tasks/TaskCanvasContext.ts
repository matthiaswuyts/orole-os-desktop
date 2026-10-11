import { createContext, useContext } from "react";
import type { ImageInfo } from "@/lib/taskEvents";

export interface TaskCanvasActions {
  openFile: (path: string) => void;
  openImage: (image: ImageInfo) => void;
  close: () => void;
}
export const TaskCanvasContext = createContext<TaskCanvasActions | null>(null);
export const useTaskCanvas = () => useContext(TaskCanvasContext);
